local cl = require "libclua"
cl.start("test.cov", 5)

function test1(i)
    if i % 2 then
        print("a "..i)
    else
        print("b "..i)
    end
end

function test2(i)
    if i > 30 then
        print("c "..i)
    else
        print("d "..i)
    end
end

function test3(i)

    if i > 0 then
        print("e "..i)
    else
        print("f "..i)
    end

end

for i = 0, 100 do
    if i < 10 then
        test1(i)
    elseif i < 50 then
        test2(i)
    else
        test3(i)
    end
end

cl.stop()
